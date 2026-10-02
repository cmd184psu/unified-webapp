// livefeed.ts — pure timing/state helpers for polling, the SSE log and first run.

export const POLL_MS = 5000
export const MAX_LOG_ENTRIES = 500

/** Poll only while visible and with no request already in flight. */
export function shouldPoll(hidden: boolean, inFlight: boolean): boolean {
  return !hidden && !inFlight
}

/** Reconnect delay for the SSE stream: 1s doubling to a 30s cap. */
export function backoffMs(attempt: number): number {
  return Math.min(30000, 1000 * 2 ** Math.max(0, attempt))
}

export function capEntries<T>(list: T[], max: number): T[] {
  return list.length > max ? list.slice(list.length - max) : list
}

export type FirstRun = 'ready' | 'import' | 'defaults'

/** Which first-run card to show. importStatus is the HTTP status of the last import attempt. */
export function firstRunState(s: { imported: boolean; importStatus?: number }): FirstRun {
  if (s.imported) return 'ready'
  return s.importStatus === 404 ? 'defaults' : 'import'
}
