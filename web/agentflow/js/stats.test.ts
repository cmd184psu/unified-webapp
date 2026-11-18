import { test, run, eq, ok, installFakeDom, FakeEl, asEl, byAttr, byField, flush, findAll } from './testkit';
import { StatsView, duration, tokensShort, type StatsDeps } from './stats';
import { dispatch } from './events';
import type { ModelStat, ProjectView, StrikeHistory } from './api';

installFakeDom();

const row = (over: Partial<ModelStat> = {}): ModelStat => ({
  model: 'qwen3-coder:30b', backend: 'ollama-m5', role: 'worker', stages: 4, passed: 2, failed: 1, infra: 1, pass_rate: 2 / 3,
  attempts_total: 3, attempts_passed: 2, total_ms: 600000, avg_ms: 150000, tokens: 1_500_000, tier: 2, family: 'qwen', strikes: 0, strikes_total: 0, benched: false, ...over,
});
const project = (slug: string): ProjectView => ({
  slug, repo_path: '/r', model_scope: 'local', feedback_cmds: [], limits: {}, created: 0, updated: 0,
  run_state: 'stopped', remembered_run_state: 'stopped', effort: null, progress: {}, current: [],
});

const noHistory: StrikeHistory = { events: [], daily: [], total: 0 };

function harness(rows: ModelStat[], history: StrikeHistory = noHistory) {
  const calls: string[] = [];
  const deps: StatsDeps = {
    stats: async (p) => { calls.push(p); return p === 'only' ? rows.slice(0, 1) : rows; },
    projects: async () => [project('only'), project('other')],
    strikes: async () => history,
    clear: async (b, m, r) => { calls.push(`clear ${b}/${m}/${r}`); },
  };
  const host = new FakeEl('DIV');
  const view = new StatsView(asEl(host), deps);
  return { host, view, calls };
}

test('the table shows time, tokens, tier and family; hostile model names stay text', async () => {
  const evil = '<img src=x onerror=alert(1)>';
  const t = harness([row({ model: evil }), row({ model: 'mistral-small3.2', family: 'mistral', role: 'reviewer', tier: 0, tokens: 900, avg_ms: 30000, total_ms: 30000, stages: 1, passed: 1, failed: 0, infra: 0, pass_rate: 1, attempts_total: 0, attempts_passed: 0 })]);
  const stop = t.view.mount();
  await flush();
  const table = byAttr(t.host, 'data-role', 'pass-rates')[0];
  const text = table.textContent;
  ok(text.includes(evil), 'literal name');
  ok(text.includes('67%') && text.includes('2/3') && text.includes('10.0m') && text.includes('1.5M') && text.includes('tier 2'), 'row: ' + text);
  ok(text.includes('not rated') && text.includes('mistral') && text.includes('100%') && text.includes('900'), 'second row: ' + text);
  ok(findAll(t.host, (e) => e.tagName === 'IMG').length === 0, 'no element injected');
  stop();
});

test('the role filter narrows the rows; the project filter asks the server', async () => {
  const t = harness([row(), row({ model: 'gpt-oss:20b', role: 'critic', family: 'gpt-oss' })]);
  const stop = t.view.mount();
  await flush();
  eq(findAll(t.host, (e) => e.tagName === 'TR').length, 3);
  const role = byField(t.host, 'role');
  role.value = 'critic';
  role.fire('change');
  ok(!byAttr(t.host, 'data-role', 'pass-rates')[0].textContent.includes('qwen3-coder'), 'worker row hidden');
  const project = byField(t.host, 'project');
  project.value = 'only';
  project.fire('change');
  await flush();
  eq(t.calls[t.calls.length - 1], 'only');
  stop();
});

test('a state change refetches; an empty result says so', async () => {
  const t = harness([]);
  const stop = t.view.mount();
  await flush();
  ok(t.host.textContent.includes('No finished stages yet'), 'empty');
  const before = t.calls.length;
  dispatch(JSON.stringify({ type: 'state.changed' }));
  await flush();
  ok(t.calls.length > before, 'refetched');
  stop();
});

test('strikes show out of three, a benched model says so, and Clear forgives it', async () => {
  const t = harness([row({ strikes: 2 }), row({ model: 'gpt-oss:20b', family: 'gpt-oss', strikes: 3, benched: true }), row({ model: 'clean' })]);
  const stop = t.view.mount();
  await flush();
  const text = byAttr(t.host, 'data-role', 'pass-rates')[0].textContent;
  ok(text.includes('2/3') && text.includes('benched'), 'strikes shown: ' + text);
  const clears = byAttr(t.host, 'data-action', 'clear-strikes');
  eq(clears.length, 2, 'only struck models get a Clear button');
  clears[1].fire('click');
  await flush();
  ok(t.calls.includes('clear ollama-m5/gpt-oss:20b/worker'), 'cleared: ' + t.calls.join('|'));
  stop();
});

test('the history lists every strike with whether it still counts, and the days', async () => {
  const at = Date.UTC(2026, 9, 15, 12);
  const t = harness([row({ strikes: 1, strikes_total: 3 })], {
    total: 3,
    events: [
      { backend: 'ollama-m5', model: 'qwen3.6:35b', role: 'worker', kind: 'repeat', task: 24, effort: 5, project: 'webcalc-local2', at, counted: true },
      { backend: 'ollama-m5', model: 'qwen3.6:35b', role: 'worker', kind: 'unparseable', at: at - 1000, counted: false, ended: 'success' },
      { backend: 'ollama-m5', model: 'qwen3.6:35b', role: 'worker', kind: 'unparseable', at: at - 2000, counted: false, ended: 'cleared' },
    ],
    daily: [{ day: '2026-10-15', backend: 'ollama-m5', model: 'qwen3.6:35b', role: 'worker', kind: 'unparseable', count: 2 }],
  });
  const stop = t.view.mount();
  await flush();
  const hist = byAttr(t.host, 'data-role', 'strike-history')[0].textContent;
  ok(hist.includes('counting') && hist.includes('ended: success') && hist.includes('ended: cleared'), 'status: ' + hist);
  ok(hist.includes('webcalc-local2 effort 5 task 24') && hist.includes('repeat'), 'where and kind: ' + hist);
  ok(byAttr(t.host, 'data-role', 'strike-days')[0].textContent.includes('2026-10-15 qwen3.6:35b worker unparseable ×2'), 'days');
  ok(byAttr(t.host, 'data-role', 'pass-rates')[0].textContent.includes('1/3') && t.host.textContent.includes('3 ever'), 'streak and lifetime total');
  stop();
});

test('duration and token formats', () => {
  eq(duration(0), '-');
  eq(duration(45000), '45s');
  eq(duration(432000), '7.2m');
  eq(duration(8_640_000), '2.4h');
  eq(tokensShort(0), '-');
  eq(tokensShort(950), '950');
  eq(tokensShort(12_400), '12.4k');
  eq(tokensShort(5_500_000), '5.5M');
});

void run('agentflow-stats');
