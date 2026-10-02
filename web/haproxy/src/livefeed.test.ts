import { test, done, eq, ok } from './testkit'
import { shouldPoll, backoffMs, capEntries, firstRunState, POLL_MS } from './livefeed'

test('poll runs only while the page is visible and nothing is in flight', () => {
  eq(shouldPoll(false, false), true)
  eq(shouldPoll(true, false), false)
  eq(shouldPoll(false, true), false)
  ok(POLL_MS >= 3000 && POLL_MS <= 10000, 'a calm interval')
})

test('SSE reconnect backoff doubles and is capped', () => {
  eq([0, 1, 2, 3, 4, 5, 6, 20].map(backoffMs), [1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000])
})

test('capEntries keeps the newest N, in order', () => {
  eq(capEntries([1, 2, 3, 4], 3), [2, 3, 4])
  eq(capEntries([1], 3), [1])
})

test('first run: import 404 offers start-with-defaults; other states do not', () => {
  eq(firstRunState({ imported: true }), 'ready')
  eq(firstRunState({ imported: false }), 'import')
  eq(firstRunState({ imported: false, importStatus: 404 }), 'defaults')
  eq(firstRunState({ imported: false, importStatus: 500 }), 'import')
  eq(firstRunState({ imported: true, importStatus: 404 }), 'ready')
})

done('livefeed')
