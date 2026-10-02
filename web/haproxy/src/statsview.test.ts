import { test, done, eq, ok } from './testkit'
import type { StatRow } from './api'
import { classifyStatus, groupByProxy, formatBytes, formatUptime, statsViewState } from './statsview'

const row = (p: Partial<StatRow>): StatRow => ({
  proxy: 'be', server: '', kind: 'server', status: 'UP', sessionsCur: 0, sessionRate: 0,
  bytesIn: 0, bytesOut: 0, checkStatus: '', lastCheck: '', ...p,
})

test('rows group by proxy in first-seen order, frontend then listeners, servers, backend last', () => {
  const g = groupByProxy([
    row({ proxy: 'fe', kind: 'frontend', status: 'OPEN' }),
    row({ proxy: 'be', server: 'app1' }),
    row({ proxy: 'be', kind: 'backend' }),
    row({ proxy: 'fe', server: 'sock-1', kind: 'listener', status: 'OPEN' }),
    row({ proxy: 'be', server: 'app2' }),
  ])
  eq(g.map(x => x.proxy), ['fe', 'be'])
  eq(g[0].rows.map(r => r.kind), ['frontend', 'listener'])
  eq(g[1].rows.map(r => r.server || r.kind), ['app1', 'app2', 'backend'])
})

test('status classification: UP, DOWN, no check, maint, other', () => {
  eq(classifyStatus(row({ status: 'UP' })), 'up')
  eq(classifyStatus(row({ status: 'UP 2/3' })), 'up')
  eq(classifyStatus(row({ status: 'OPEN', kind: 'frontend' })), 'up')
  eq(classifyStatus(row({ status: 'DOWN' })), 'down')
  eq(classifyStatus(row({ status: 'DOWN 1/2' })), 'down')
  eq(classifyStatus(row({ status: 'no check' })), 'nocheck')
  eq(classifyStatus(row({ status: 'MAINT' })), 'maint')
  eq(classifyStatus(row({ status: 'MAINT (via be/app1)' })), 'maint')
  eq(classifyStatus(row({ status: 'DRAIN' })), 'other')
})

test('bytes format humanly', () => {
  eq(formatBytes(0), '0 B')
  eq(formatBytes(512), '512 B')
  eq(formatBytes(1536), '1.5 KiB')
  eq(formatBytes(1048576), '1.0 MiB')
  eq(formatBytes(5 * 1024 ** 3), '5.0 GiB')
})

test('uptime formats humanly', () => {
  eq(formatUptime(45), '45s')
  eq(formatUptime(303), '5m 3s')
  eq(formatUptime(3 * 3600 + 120), '3h 2m')
  eq(formatUptime(184361), '2d 3h 12m')
  eq(formatUptime(-1), '0s')
})

test('view state: loading, unavailable keeps the plain message, ready', () => {
  eq(statsViewState(null).kind, 'loading')
  const u = statsViewState({ available: false, message: 'HAProxy is stopped.', info: { version: '', uptimeSec: 0, currConns: 0, pid: 0 }, rows: [] })
  eq(u.kind, 'unavailable')
  eq(u.message, 'HAProxy is stopped.')
  const r = statsViewState({ available: true, message: '', info: { version: '3.0', uptimeSec: 1, currConns: 2, pid: 3 }, rows: [] })
  eq(r.kind, 'ready')
  ok(statsViewState({ available: false, message: '', info: { version: '', uptimeSec: 0, currConns: 0, pid: 0 }, rows: [] }).message.length > 0, 'never an empty message')
})

done('statsview')
